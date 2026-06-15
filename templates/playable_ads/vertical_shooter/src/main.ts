import Phaser from "phaser";
import "./styles.css";

const GAME_WIDTH = 390;
const GAME_HEIGHT = 844;
const STORE_URL = "https://play.google.com/store";
const BOSS_TRIGGER_KILLS = 28;
const MISSION_GOAL = BOSS_TRIGGER_KILLS + 1;

type Enemy = Phaser.Physics.Arcade.Image & {
  hp: number;
  scoreValue: number;
  baseX: number;
  wave: number;
  phase: number;
};

type Bullet = Phaser.Physics.Arcade.Image & {
  damage: number;
};

type PowerUp = Phaser.Physics.Arcade.Image & {
  kind: "spread" | "shield";
};

declare global {
  interface Window {
    mraid?: {
      open: (url: string) => void;
    };
  }
}

class BootScene extends Phaser.Scene {
  constructor() {
    super("Boot");
  }

  preload() {
    this.load.image("desert", "assets/generated/desert-bg-loop.png");
    this.load.image("player", "assets/generated/player.png");
    this.load.image("enemy", "assets/generated/enemy.png");
    this.load.image("boss", "assets/generated/boss.png");
    this.load.image("powerShield", "assets/generated/power-shield.png");
    this.load.image("powerSpread", "assets/generated/power-firepower.png");
  }

  create() {
    this.createCircleTexture("bullet", 0x75e8ff, 10, 22);
    this.createCircleTexture("enemyBullet", 0xff6b45, 12, 12);
    this.createCircleTexture("spark", 0xfff3a6, 10, 10);
    this.scene.start("Game");
  }

  private createDesertTexture() {
    const graphics = this.add.graphics();
    graphics.fillStyle(0xd9954d, 1);
    graphics.fillRect(0, 0, GAME_WIDTH, GAME_HEIGHT);
    graphics.fillStyle(0xc77735, 0.55);
    graphics.fillTriangle(0, 80, 120, 0, 0, 0);
    graphics.fillTriangle(GAME_WIDTH, 0, 265, 0, GAME_WIDTH, 130);
    graphics.fillStyle(0xf0b76d, 0.9);
    graphics.fillEllipse(92, 160, 210, 64);
    graphics.fillEllipse(310, 520, 240, 76);
    graphics.fillEllipse(140, 725, 270, 70);

    graphics.fillStyle(0xa95d2f, 0.28);
    for (let y = -40; y < GAME_HEIGHT + 120; y += 88) {
      graphics.fillEllipse(58, y, 118, 32);
      graphics.fillEllipse(336, y + 44, 104, 28);
    }

    graphics.fillStyle(0x8e4b2e, 0.45);
    for (let i = 0; i < 46; i += 1) {
      const x = Phaser.Math.Between(12, GAME_WIDTH - 12);
      const y = Phaser.Math.Between(0, GAME_HEIGHT);
      graphics.fillRoundedRect(x, y, Phaser.Math.Between(14, 42), Phaser.Math.Between(3, 7), 3);
    }

    graphics.lineStyle(3, 0x7d442a, 0.2);
    for (let y = 28; y < GAME_HEIGHT; y += 64) {
      graphics.beginPath();
      graphics.moveTo(0, y);
      graphics.lineTo(GAME_WIDTH, y + Phaser.Math.Between(-18, 18));
      graphics.strokePath();
    }

    graphics.generateTexture("desert", GAME_WIDTH, GAME_HEIGHT);
    graphics.destroy();
  }

  private createCircleTexture(key: string, color: number, width: number, height: number) {
    const graphics = this.add.graphics();
    graphics.fillStyle(color, 1);
    graphics.fillRoundedRect(0, 0, width, height, Math.min(width, height) / 2);
    graphics.fillStyle(0xffffff, 0.75);
    graphics.fillRoundedRect(width * 0.25, height * 0.08, width * 0.5, height * 0.25, width * 0.2);
    graphics.generateTexture(key, width, height);
    graphics.destroy();
  }

  private createPlayerTexture() {
    const graphics = this.add.graphics();
    graphics.fillStyle(0x0f2f45, 0.32);
    graphics.fillEllipse(45, 92, 70, 18);
    graphics.fillStyle(0xdce8e6, 1);
    graphics.fillTriangle(45, 0, 24, 82, 66, 82);
    graphics.fillStyle(0x2bb7b0, 1);
    graphics.fillTriangle(45, 13, 5, 66, 28, 75);
    graphics.fillTriangle(45, 13, 85, 66, 62, 75);
    graphics.fillStyle(0x1f6f78, 1);
    graphics.fillRoundedRect(34, 34, 22, 46, 8);
    graphics.fillStyle(0xffc743, 1);
    graphics.fillTriangle(32, 76, 58, 76, 45, 104);
    graphics.fillStyle(0x86f8ff, 0.9);
    graphics.fillEllipse(45, 31, 17, 24);
    graphics.lineStyle(3, 0xffffff, 0.65);
    graphics.lineBetween(45, 8, 45, 76);
    graphics.generateTexture("player", 90, 108);
    graphics.destroy();
  }

  private createEnemyTexture() {
    const graphics = this.add.graphics();
    graphics.fillStyle(0x28140d, 0.28);
    graphics.fillEllipse(36, 64, 60, 14);
    graphics.fillStyle(0x8b2d24, 1);
    graphics.fillTriangle(36, 78, 15, 13, 57, 13);
    graphics.fillStyle(0xd4562f, 1);
    graphics.fillTriangle(36, 48, 0, 18, 19, 6);
    graphics.fillTriangle(36, 48, 72, 18, 53, 6);
    graphics.fillStyle(0xf2ad48, 1);
    graphics.fillRoundedRect(25, 17, 22, 32, 8);
    graphics.fillStyle(0x28140d, 0.55);
    graphics.fillEllipse(36, 30, 16, 11);
    graphics.fillStyle(0xffcf68, 1);
    graphics.fillTriangle(27, 68, 45, 68, 36, 88);
    graphics.generateTexture("enemy", 72, 92);
    graphics.destroy();
  }

  private createBossTexture() {
    const graphics = this.add.graphics();
    graphics.fillStyle(0x2b160f, 0.3);
    graphics.fillEllipse(82, 118, 140, 26);
    graphics.fillStyle(0x5c3030, 1);
    graphics.fillRoundedRect(23, 18, 118, 76, 20);
    graphics.fillStyle(0xb23f2c, 1);
    graphics.fillTriangle(82, 108, 0, 36, 29, 122);
    graphics.fillTriangle(82, 108, 164, 36, 135, 122);
    graphics.fillStyle(0xf0a33d, 1);
    graphics.fillRoundedRect(55, 8, 54, 82, 18);
    graphics.fillStyle(0x3d1e1a, 0.65);
    graphics.fillCircle(82, 49, 21);
    graphics.fillStyle(0xffe074, 1);
    graphics.fillCircle(82, 49, 10);
    graphics.fillStyle(0xf8d2a4, 0.88);
    graphics.fillCircle(75, 42, 4);
    graphics.lineStyle(5, 0xffd36a, 0.8);
    graphics.lineBetween(28, 38, 136, 38);
    graphics.generateTexture("boss", 164, 132);
    graphics.destroy();
  }

  private createPowerTextures() {
    const shield = this.add.graphics();
    shield.fillStyle(0x093243, 0.5);
    shield.fillCircle(24, 24, 24);
    shield.lineStyle(3, 0x8ff4ff, 0.95);
    shield.strokeCircle(24, 24, 20);
    shield.fillStyle(0x5ee7ff, 1);
    shield.fillTriangle(24, 9, 10, 18, 14, 34);
    shield.fillTriangle(24, 9, 38, 18, 34, 34);
    shield.fillRoundedRect(15, 18, 18, 20, 5);
    shield.fillStyle(0xffffff, 0.72);
    shield.fillEllipse(21, 17, 8, 5);
    shield.generateTexture("powerShield", 48, 48);
    shield.destroy();

    const spread = this.add.graphics();
    spread.fillStyle(0x3f2108, 0.48);
    spread.fillCircle(24, 24, 24);
    spread.lineStyle(3, 0xffe483, 0.95);
    spread.strokeCircle(24, 24, 20);
    spread.fillStyle(0xffd052, 1);
    spread.fillTriangle(24, 6, 18, 28, 30, 28);
    spread.fillTriangle(10, 18, 12, 38, 24, 28);
    spread.fillTriangle(38, 18, 36, 38, 24, 28);
    spread.fillStyle(0xffffff, 0.85);
    spread.fillCircle(24, 18, 4);
    spread.generateTexture("powerSpread", 48, 48);
    spread.destroy();
  }
}

class GameScene extends Phaser.Scene {
  private player!: Phaser.Physics.Arcade.Image;
  private shield!: Phaser.GameObjects.Arc;
  private desertBack!: Phaser.GameObjects.TileSprite;
  private dustLayer!: Phaser.GameObjects.TileSprite;
  private bullets!: Phaser.Physics.Arcade.Group;
  private enemies!: Phaser.Physics.Arcade.Group;
  private enemyBullets!: Phaser.Physics.Arcade.Group;
  private powerUps!: Phaser.Physics.Arcade.Group;
  private scoreText!: Phaser.GameObjects.Text;
  private hpBarFill!: Phaser.GameObjects.Rectangle;
  private progressText!: Phaser.GameObjects.Text;
  private progressFill!: Phaser.GameObjects.Rectangle;
  private dragOffset = new Phaser.Math.Vector2();
  private score = 0;
  private hp = 3;
  private kills = 0;
  private goal = MISSION_GOAL;
  private fireLevel = 1;
  private nextShotAt = 0;
  private nextEnemyAt = 0;
  private bossSpawned = false;
  private gameEnded = false;

  constructor() {
    super("Game");
  }

  create() {
    this.physics.world.setBounds(0, 0, GAME_WIDTH, GAME_HEIGHT);
    this.desertBack = this.add.tileSprite(0, 0, GAME_WIDTH, GAME_HEIGHT, "desert").setOrigin(0);
    this.dustLayer = this.add.tileSprite(0, 0, GAME_WIDTH, GAME_HEIGHT, "desert").setOrigin(0);
    this.dustLayer.setBlendMode(Phaser.BlendModes.SCREEN).setAlpha(0.18).setTint(0xffd29a);

    this.bullets = this.physics.add.group({ classType: Phaser.Physics.Arcade.Image, maxSize: 70 });
    this.enemies = this.physics.add.group({ classType: Phaser.Physics.Arcade.Image, maxSize: 28 });
    this.enemyBullets = this.physics.add.group({ classType: Phaser.Physics.Arcade.Image, maxSize: 36 });
    this.powerUps = this.physics.add.group({ classType: Phaser.Physics.Arcade.Image, maxSize: 4 });

    this.player = this.physics.add.image(GAME_WIDTH / 2, GAME_HEIGHT - 122, "player");
    this.player.setDisplaySize(78, 87);
    this.player.setCircle(34, 44, 47).setDepth(10).setCollideWorldBounds(true);
    this.shield = this.add.circle(this.player.x, this.player.y + 4, 46, 0x84f7ff, 0.13);
    this.shield.setStrokeStyle(2, 0x84f7ff, 0.65).setDepth(9).setVisible(false);

    this.createHud();
    this.setupInput();
    this.setupCollisions();

    this.add.text(GAME_WIDTH / 2, 142, "拖动战机，自动射击", {
      color: "#fff1c8",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "18px",
      fontStyle: "bold",
      stroke: "#5b2d18",
      strokeThickness: 4,
    }).setOrigin(0.5).setAlpha(0.94);
  }

  update(time: number, delta: number) {
    if (this.gameEnded) {
      return;
    }

    this.desertBack.tilePositionY -= delta * 0.055;
    this.dustLayer.tilePositionY -= delta * 0.16;
    this.dustLayer.tilePositionX += delta * 0.012;
    this.shield.setPosition(this.player.x, this.player.y + 4);

    if (time > this.nextShotAt) {
      this.fire();
      this.nextShotAt = time + Math.max(115, 190 - this.fireLevel * 18);
    }

    if (time > this.nextEnemyAt && !this.bossSpawned) {
      this.spawnEnemy();
      this.nextEnemyAt = time + Phaser.Math.Between(700, 1040);
    }

    if (this.kills >= BOSS_TRIGGER_KILLS && !this.bossSpawned) {
      this.spawnBoss();
    }

    this.enemies.children.iterate((child) => {
      const enemy = child as Enemy | undefined;
      if (!enemy?.active) {
        return true;
      }
      enemy.x = enemy.baseX + Math.sin(time * 0.0024 + enemy.phase) * enemy.wave;
      if (enemy.y > GAME_HEIGHT + 80) {
        enemy.destroy();
      }
      return true;
    });

    this.cleanupGroup(this.bullets, -80, GAME_HEIGHT + 80);
    this.cleanupGroup(this.enemyBullets, -80, GAME_HEIGHT + 80);
    this.cleanupGroup(this.powerUps, -80, GAME_HEIGHT + 80);
  }

  private createHud() {
    this.add.rectangle(0, 0, GAME_WIDTH, 106, 0x301a10, 0.56).setOrigin(0);
    this.add.rectangle(0, 98, GAME_WIDTH, 3, 0xffcf76, 0.45).setOrigin(0);
    this.add.text(20, 18, "SKY STRIKE", {
      color: "#fff3cf",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "24px",
      fontStyle: "900",
    });
    this.scoreText = this.add.text(22, 54, "SCORE 0", {
      color: "#ffe0a0",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "15px",
      fontStyle: "bold",
    });

    this.add.text(GAME_WIDTH - 22, 17, "ARMOR", {
      color: "#fff0bd",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "14px",
      fontStyle: "bold",
    }).setOrigin(1, 0);
    this.add.rectangle(GAME_WIDTH - 132, 39, 110, 13, 0x31140f, 1).setOrigin(0).setStrokeStyle(2, 0xffdf91, 0.7);
    this.hpBarFill = this.add.rectangle(GAME_WIDTH - 129, 42, 104, 7, 0x3cff75, 1).setOrigin(0);

    this.progressText = this.add.text(GAME_WIDTH - 22, 51, `0/${MISSION_GOAL}`, {
      color: "#ffffff",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "15px",
      fontStyle: "bold",
    }).setOrigin(1, 0);
    this.add.rectangle(20, 84, GAME_WIDTH - 40, 8, 0x5f341e, 1).setOrigin(0);
    this.progressFill = this.add.rectangle(20, 84, 0, 8, 0xffd452, 1).setOrigin(0);
  }

  private setupInput() {
    this.input.on("pointerdown", (pointer: Phaser.Input.Pointer) => {
      this.dragOffset.set(this.player.x - pointer.x, this.player.y - pointer.y);
      this.movePlayer(pointer);
    });

    this.input.on("pointermove", (pointer: Phaser.Input.Pointer) => {
      if (pointer.isDown) {
        this.movePlayer(pointer);
      }
    });
  }

  private movePlayer(pointer: Phaser.Input.Pointer) {
    const x = Phaser.Math.Clamp(pointer.x + this.dragOffset.x, 38, GAME_WIDTH - 38);
    const y = Phaser.Math.Clamp(pointer.y + this.dragOffset.y, 150, GAME_HEIGHT - 72);
    this.tweens.add({
      targets: this.player,
      x,
      y,
      duration: 55,
      ease: "Sine.easeOut",
    });
  }

  private setupCollisions() {
    this.physics.add.overlap(this.bullets, this.enemies, (bulletObj, enemyObj) => {
      this.hitEnemy(bulletObj as Bullet, enemyObj as Enemy);
    });
    this.physics.add.overlap(this.player, this.enemies, (_playerObj, enemyObj) => {
      this.damagePlayer(enemyObj as Enemy);
    });
    this.physics.add.overlap(this.player, this.enemyBullets, (_playerObj, bulletObj) => {
      bulletObj.destroy();
      this.damagePlayer();
    });
    this.physics.add.overlap(this.player, this.powerUps, (_playerObj, powerObj) => {
      this.collectPowerUp(powerObj as PowerUp);
    });
  }

  private fire() {
    const offsets = this.fireLevel >= 3 ? [-24, 0, 24] : this.fireLevel >= 2 ? [-14, 14] : [0];
    offsets.forEach((offset) => {
      const bullet = this.bullets.get(this.player.x + offset, this.player.y - 42, "bullet") as Bullet | null;
      if (!bullet) {
        return;
      }
      bullet.damage = 1;
      bullet.setActive(true).setVisible(true).setDepth(6);
      bullet.setSize(8, 20).setVelocity(offset * 1.3, -720);
    });
  }

  private spawnEnemy() {
    const x = Phaser.Math.Between(46, GAME_WIDTH - 46);
    const enemy = this.enemies.get(x, -58, "enemy") as Enemy | null;
    if (!enemy) {
      return;
    }
    enemy.hp = Phaser.Math.Between(1, 2);
    enemy.scoreValue = enemy.hp * 100;
    enemy.baseX = x;
    enemy.wave = Phaser.Math.Between(12, 54);
    enemy.phase = Phaser.Math.FloatBetween(0, Math.PI * 2);
    enemy.setActive(true).setVisible(true).setDepth(5);
    enemy.setDisplaySize(66, 76);
    enemy.setCircle(28, 31, 34);
    enemy.setVelocity(0, Phaser.Math.Between(115, 190));

    if (Phaser.Math.Between(0, 100) < 36) {
      this.time.delayedCall(700, () => this.enemyShoot(enemy));
    }
  }

  private spawnBoss() {
    this.bossSpawned = true;
    this.goal = MISSION_GOAL;
    const boss = this.enemies.get(GAME_WIDTH / 2, -95, "boss") as Enemy | null;
    if (!boss) {
      return;
    }
    boss.hp = 46;
    boss.scoreValue = 3600;
    boss.baseX = GAME_WIDTH / 2;
    boss.wave = 88;
    boss.phase = 0;
    boss.setActive(true).setVisible(true).setDepth(5);
    boss.setDisplaySize(148, 174);
    boss.setCircle(58, 86, 96);
    boss.setVelocity(0, 52);
    this.tweens.add({
      targets: boss,
      y: 126,
      duration: 1300,
      ease: "Back.easeOut",
      onComplete: () => boss.setVelocity(0, 0),
    });
    this.time.addEvent({
      delay: 760,
      repeat: 21,
      callback: () => this.enemyShoot(boss, true),
    });
    this.progressText.setText(`${this.kills}/${this.goal}`);
  }

  private enemyShoot(enemy: Enemy, spread = false) {
    if (!enemy.active || this.gameEnded) {
      return;
    }
    const angles = spread ? [-70, -90, -110] : [-90];
    angles.forEach((angle) => {
      const bullet = this.enemyBullets.get(enemy.x, enemy.y + 32, "enemyBullet") as Phaser.Physics.Arcade.Image | null;
      if (!bullet) {
        return;
      }
      this.physics.velocityFromAngle(-angle, 180, bullet.body?.velocity);
      bullet.setActive(true).setVisible(true).setDepth(4);
      bullet.setCircle(6);
    });
  }

  private hitEnemy(bullet: Bullet, enemy: Enemy) {
    bullet.destroy();
    enemy.hp -= bullet.damage;
    this.flash(enemy);
    this.spawnSparks(bullet.x, bullet.y, 5);
    if (enemy.hp > 0) {
      return;
    }
    this.score += enemy.scoreValue;
    this.kills += 1;
    const isBoss = enemy.texture.key === "boss";
    this.spawnSparks(enemy.x, enemy.y, isBoss ? 42 : 16);
    enemy.destroy();
    this.cameras.main.shake(isBoss ? 220 : 80, isBoss ? 0.012 : 0.004);
    if (!isBoss && Phaser.Math.Between(0, 100) < 24) {
      this.spawnPowerUp(enemy.x, enemy.y);
    }
    this.updateHud();
    if (this.bossSpawned && isBoss) {
      this.finish(true);
    }
  }

  private spawnPowerUp(x: number, y: number) {
    const kind: PowerUp["kind"] = Phaser.Math.Between(0, 1) === 0 ? "spread" : "shield";
    const texture = kind === "spread" ? "powerSpread" : "powerShield";
    const power = this.powerUps.get(x, y, texture) as PowerUp | null;
    if (!power) {
      return;
    }
    power.kind = kind;
    power.setActive(true).setVisible(true).setDepth(7);
    power.setTexture(texture);
    power.setDisplaySize(48, 49);
    power.setCircle(24, 13, 13);
    power.setVelocity(0, 150);
    power.clearTint();
    this.tweens.add({
      targets: power,
      angle: 360,
      duration: 900,
      repeat: -1,
    });
  }

  private collectPowerUp(power: PowerUp) {
    if (power.kind === "spread") {
      this.fireLevel = Math.min(3, this.fireLevel + 1);
    } else {
      this.shield.setVisible(true);
      this.time.delayedCall(3800, () => this.shield.setVisible(false));
    }
    this.spawnSparks(power.x, power.y, 18);
    power.destroy();
  }

  private damagePlayer(enemy?: Enemy) {
    if (this.shield.visible) {
      if (enemy) {
        this.spawnSparks(enemy.x, enemy.y, 14);
        enemy.destroy();
      }
      this.shield.setVisible(false);
      return;
    }

    if (enemy) {
      enemy.destroy();
    }
    this.hp -= 1;
    this.updateHud();
    this.cameras.main.shake(180, 0.014);
    this.flash(this.player);
    if (this.hp <= 0) {
      this.finish(false);
    }
  }

  private updateHud() {
    this.scoreText.setText(`SCORE ${this.score}`);
    this.progressText.setText(`${this.kills}/${this.goal}`);
    this.progressFill.width = (GAME_WIDTH - 40) * Phaser.Math.Clamp(this.kills / this.goal, 0, 1);
    this.hpBarFill.width = 104 * Phaser.Math.Clamp(this.hp / 3, 0, 1);
    this.hpBarFill.fillColor = this.hp <= 1 ? 0xff4e35 : this.hp === 2 ? 0xffc83d : 0x3cff75;
  }

  private finish(victory: boolean) {
    if (this.gameEnded) {
      return;
    }
    this.gameEnded = true;
    this.physics.pause();

    const shade = this.add.rectangle(0, 0, GAME_WIDTH, GAME_HEIGHT, 0x2b160d, 0.72).setOrigin(0).setDepth(30);
    const title = victory ? "空域清理完成" : "差一点就赢了";
    const copy = victory ? "升级火力，挑战下一波 Boss" : "领取强化战机，再来一局";
    this.add.text(GAME_WIDTH / 2, 228, title, {
      color: "#ffffff",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "34px",
      fontStyle: "900",
      align: "center",
      stroke: "#12295d",
      strokeThickness: 6,
    }).setOrigin(0.5).setDepth(31);
    this.add.text(GAME_WIDTH / 2, 286, copy, {
      color: "#bfefff",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "18px",
      fontStyle: "bold",
      align: "center",
    }).setOrigin(0.5).setDepth(31);

    const cta = this.add.rectangle(GAME_WIDTH / 2, 382, 236, 66, 0xffd347, 1)
      .setStrokeStyle(4, 0xffffff, 0.75)
      .setDepth(31)
      .setInteractive({ useHandCursor: true });
    const ctaText = this.add.text(GAME_WIDTH / 2, 382, "立即下载", {
      color: "#572900",
      fontFamily: "Arial, Helvetica, sans-serif",
      fontSize: "26px",
      fontStyle: "900",
    }).setOrigin(0.5).setDepth(32);

    this.tweens.add({
      targets: [cta, ctaText],
      scale: 1.06,
      duration: 520,
      yoyo: true,
      repeat: -1,
      ease: "Sine.easeInOut",
    });
    cta.on("pointerup", () => this.openStore());
    shade.setInteractive().on("pointerup", () => this.openStore());
  }

  private openStore() {
    if (window.mraid) {
      window.mraid.open(STORE_URL);
      return;
    }
    window.open(STORE_URL, "_blank", "noopener,noreferrer");
  }

  private flash(target: Phaser.GameObjects.GameObject) {
    this.tweens.add({
      targets: target,
      alpha: 0.35,
      duration: 48,
      yoyo: true,
      repeat: 1,
    });
  }

  private spawnSparks(x: number, y: number, count: number) {
    for (let i = 0; i < count; i += 1) {
      const spark = this.add.image(x, y, "spark").setDepth(20);
      const angle = Phaser.Math.FloatBetween(0, Math.PI * 2);
      const distance = Phaser.Math.Between(18, 76);
      this.tweens.add({
        targets: spark,
        x: x + Math.cos(angle) * distance,
        y: y + Math.sin(angle) * distance,
        alpha: 0,
        scale: 0.15,
        duration: Phaser.Math.Between(260, 520),
        ease: "Quad.easeOut",
        onComplete: () => spark.destroy(),
      });
    }
  }

  private cleanupGroup(group: Phaser.Physics.Arcade.Group, minY: number, maxY: number) {
    group.children.iterate((child) => {
      const body = child as Phaser.Physics.Arcade.Image | undefined;
      if (body?.active && (body.y < minY || body.y > maxY)) {
        body.destroy();
      }
      return true;
    });
  }
}

const config: Phaser.Types.Core.GameConfig = {
  type: Phaser.AUTO,
  parent: "game",
  width: GAME_WIDTH,
  height: GAME_HEIGHT,
  backgroundColor: "#07122c",
  scale: {
    mode: Phaser.Scale.FIT,
    autoCenter: Phaser.Scale.CENTER_BOTH,
  },
  physics: {
    default: "arcade",
    arcade: {
      debug: false,
    },
  },
  scene: [BootScene, GameScene],
};

new Phaser.Game(config);
